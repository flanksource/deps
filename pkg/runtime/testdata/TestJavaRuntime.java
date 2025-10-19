public class TestJavaRuntime {
    public static void main(String[] args) {
        System.out.println("Java Runtime Test");
        System.out.println("Java version: " + System.getProperty("java.version"));
        System.out.println("Working directory: " + System.getProperty("user.dir"));

        // Check for environment variables
        String apiKey = System.getenv("TEST_API_KEY");
        if (apiKey == null) {
            apiKey = "not_set";
        }
        System.out.println("TEST_API_KEY: " + apiKey);

        System.out.println("Test completed successfully!");
    }
}
